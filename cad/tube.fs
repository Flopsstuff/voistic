FeatureScript 3112;
import(path : "onshape/std/common.fs", version : "3112.0");

// The body of the voistic stick: a tube closed at the bottom, printed standing on that bottom. The long edges are
// round, which costs nothing when printed standing, and the foot starts as a 45 degree chamfer so it needs no support.
// Everything goes in from the top, and a cap with a pressing plate closes it. The front is the -Y side.
annotation { "Feature Type Name" : "Voistic Tube", "Feature Type Description" : "Body of the voice stick, printed standing" }
export const voisticTube = defineFeature(function(context is Context, id is Id, definition is map)
    precondition
    {
        annotation { "Name" : "Full length, with the cap" }
        isLength(definition.length, { (millimeter) : [20, 80, 300] } as LengthBoundSpec);
        annotation { "Name" : "Width" }
        isLength(definition.width, { (millimeter) : [10, 28, 100] } as LengthBoundSpec);
        annotation { "Name" : "Depth" }
        isLength(definition.depth, { (millimeter) : [5, 20, 100] } as LengthBoundSpec);
        annotation { "Name" : "Cap height, outside the tube" }
        isLength(definition.capHeight, { (millimeter) : [0, 6, 50] } as LengthBoundSpec);
        annotation { "Name" : "Wall" }
        isLength(definition.wall, { (millimeter) : [0.8, 1.8, 5] } as LengthBoundSpec);
        annotation { "Name" : "Floor" }
        isLength(definition.floor, { (millimeter) : [0.8, 2, 10] } as LengthBoundSpec);
        annotation { "Name" : "Long edge radius" }
        isLength(definition.edgeRadius, { (millimeter) : [1, 7, 30] } as LengthBoundSpec);
        annotation { "Name" : "Foot chamfer" }
        isLength(definition.footChamfer, { (millimeter) : [0.2, 0.8, 5] } as LengthBoundSpec);
        annotation { "Name" : "Foot radius" }
        isLength(definition.footRadius, { (millimeter) : [0.2, 3, 10] } as LengthBoundSpec);
        annotation { "Name" : "Inside chamfer between the floor and the walls" }
        isLength(definition.floorChamfer, { (millimeter) : [0.2, 1.5, 5] } as LengthBoundSpec);

        annotation { "Name" : "Button diameter" }
        isLength(definition.buttonDiameter, { (millimeter) : [1, 6.7, 20] } as LengthBoundSpec);
        annotation { "Name" : "Button clearance, on the diameter" }
        isLength(definition.buttonClearance, { (millimeter) : [0, 0.4, 2] } as LengthBoundSpec);
        annotation { "Name" : "Button from the top of the tube" }
        isLength(definition.buttonCentreFromTop, { (millimeter) : [1, 18, 200] } as LengthBoundSpec);
        annotation { "Name" : "Button square" }
        isLength(definition.squareSize, { (millimeter) : [1, 12, 30] } as LengthBoundSpec);
        annotation { "Name" : "Square clearance, on the size" }
        isLength(definition.squareClearance, { (millimeter) : [0, 0.4, 2] } as LengthBoundSpec);
        annotation { "Name" : "Square pocket depth" }
        isLength(definition.squarePocket, { (millimeter) : [0, 0.6, 3] } as LengthBoundSpec);

        annotation { "Name" : "Vertical slit, button centre to its bottom end" }
        isLength(definition.slitFromButton, { (millimeter) : [1, 8, 100] } as LengthBoundSpec);
        annotation { "Name" : "Vertical slit, height" }
        isLength(definition.slitHeight, { (millimeter) : [0.5, 6, 100] } as LengthBoundSpec);
        annotation { "Name" : "Vertical slit, width" }
        isLength(definition.slitWidth, { (millimeter) : [0.3, 1, 5] } as LengthBoundSpec);
        annotation { "Name" : "Vertical slit, chamfer on both faces" }
        isLength(definition.slitChamfer, { (millimeter) : [0.1, 0.5, 1] } as LengthBoundSpec);

        annotation { "Name" : "Snap dimple from the top of the tube" }
        isLength(definition.snapFromTop, { (millimeter) : [1, 18, 200] } as LengthBoundSpec);
        annotation { "Name" : "Snap dimple depth" }
        isLength(definition.snapDepth, { (millimeter) : [0.1, 0.6, 3] } as LengthBoundSpec);
        annotation { "Name" : "Snap dimple diameter" }
        isLength(definition.snapDiameter, { (millimeter) : [0.5, 3.4, 10] } as LengthBoundSpec);
    }
    {
        const w = definition.width;
        const d = definition.depth;
        const t = definition.wall;
        const rc = definition.edgeRadius;
        const h = definition.length - definition.capHeight;
        const front = -d / 2;
        const inner = front + t;

        if (2 * t >= d || 2 * t >= w)
            throw regenError("Wall is too thick for this size", ["wall"]);
        if (rc >= d / 2 || rc <= t)
            throw regenError("Long edge radius must be above the wall and below half the depth", ["edgeRadius"]);
        if (h <= definition.floor + definition.footRadius)
            throw regenError("The cap leaves no tube", ["capHeight"]);
        if (definition.squarePocket >= t)
            throw regenError("Square pocket must be shallower than the wall", ["squarePocket"]);
        if (definition.snapDepth >= t)
            throw regenError("The snap dimple must be shallower than the wall", ["snapDepth"]);
        if (definition.squareSize + definition.squareClearance > w - 2 * rc)
            throw regenError("The button square is wider than the flat of the front wall", ["squareSize"]);
        if (definition.slitWidth > w - 2 * rc)
            throw regenError("The slit is wider than the flat of the front wall", ["slitWidth"]);
        if (definition.slitHeight <= definition.slitWidth)
            throw regenError("The slit must be taller than it is wide", ["slitHeight"]);
        if (2 * definition.slitChamfer >= t)
            throw regenError("The slit chamfers leave no wall", ["slitChamfer"]);
        if (definition.buttonCentreFromTop - definition.slitFromButton - definition.slitHeight <= definition.slitChamfer)
            throw regenError("The slit runs out of the top of the tube", ["slitFromButton"]);

        // Outside: a block with round long edges, then the foot, a 45 degree chamfer that a radius runs into
        fCuboid(context, id + "outer", {
                    "corner1" : vector(-w / 2, -d / 2, 0 * millimeter),
                    "corner2" : vector(w / 2, d / 2, h) });
        const body = qCreatedBy(id + "outer", EntityType.BODY);
        opFillet(context, id + "longEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(body, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc });
        const bottom = qCoincidesWithPlane(qOwnedByBody(body, EntityType.FACE), plane(vector(0, 0, 0) * millimeter, vector(0, 0, 1)));
        opChamfer(context, id + "footChamfer", {
                    "entities" : qAdjacent(bottom, AdjacencyType.EDGE, EntityType.EDGE),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.footChamfer });
        opFillet(context, id + "footRadius", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(body, EntityType.EDGE),
                            plane(vector(0, 0, 1) * definition.footChamfer, vector(0, 0, 1))),
                    "radius" : definition.footRadius });

        // Inside: the same section, a wall smaller, down to the floor
        fCuboid(context, id + "inner", {
                    "corner1" : vector(-w / 2 + t, -d / 2 + t, definition.floor),
                    "corner2" : vector(w / 2 - t, d / 2 - t, h + 1 * millimeter) });
        const cavity = qCreatedBy(id + "inner", EntityType.BODY);
        opFillet(context, id + "innerEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(cavity, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc - t });
        // A chamfer inside, between the floor and the walls, where the outside already has one
        opChamfer(context, id + "floorChamfer", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(cavity, EntityType.EDGE), plane(vector(0 * millimeter, 0 * millimeter, definition.floor), vector(0, 0, 1))),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.floorChamfer });
        var tools = [cavity];

        // Front wall: button hole, its square seat from inside, a vertical slit for the microphone
        const buttonZ = h - definition.buttonCentreFromTop;
        fCylinder(context, id + "button", {
                    "bottomCenter" : vector(0 * millimeter, front - 1 * millimeter, buttonZ),
                    "topCenter" : vector(0 * millimeter, inner + 1 * millimeter, buttonZ),
                    "radius" : (definition.buttonDiameter + definition.buttonClearance) / 2 });
        tools = append(tools, qCreatedBy(id + "button", EntityType.BODY));
        // The slit has full round ends, so it is a block and two cylinders
        const slitR = definition.slitWidth / 2;
        const slitLow = buttonZ + definition.slitFromButton + slitR;
        const slitHigh = buttonZ + definition.slitFromButton + definition.slitHeight - slitR;
        fCuboid(context, id + "slit", {
                    "corner1" : vector(-slitR, front - 1 * millimeter, slitLow),
                    "corner2" : vector(slitR, inner + 1 * millimeter, slitHigh) });
        tools = append(tools, qCreatedBy(id + "slit", EntityType.BODY));
        var slitEnd = 0;
        for (var z in [slitLow, slitHigh])
        {
            fCylinder(context, id + ("slitEnd" ~ slitEnd), {
                        "bottomCenter" : vector(0 * millimeter, front - 1 * millimeter, z),
                        "topCenter" : vector(0 * millimeter, inner + 1 * millimeter, z),
                        "radius" : slitR });
            tools = append(tools, qCreatedBy(id + ("slitEnd" ~ slitEnd), EntityType.BODY));
            slitEnd += 1;
        }
        if (definition.squarePocket > 0 * millimeter)
        {
            const half = (definition.squareSize + definition.squareClearance) / 2;
            fCuboid(context, id + "square", {
                        "corner1" : vector(-half, inner - definition.squarePocket, buttonZ - half),
                        "corner2" : vector(half, inner + 1 * millimeter, buttonZ + half) });
            tools = append(tools, qCreatedBy(id + "square", EntityType.BODY));
        }

        // Back wall: the dimple the bump on the cap's plate snaps into, a cap of a sphere
        const snapHalf = definition.snapDiameter / 2;
        const snapR = (snapHalf * snapHalf + definition.snapDepth * definition.snapDepth) / (2 * definition.snapDepth);
        opPoint(context, id + "snapCentre", { "point" : vector(0 * millimeter, d / 2 - t + definition.snapDepth - snapR, h - definition.snapFromTop) });
        fSphere(context, id + "snap", { "center" : qCreatedBy(id + "snapCentre", EntityType.VERTEX), "radius" : snapR });
        opDeleteBodies(context, id + "snapCentreGone", { "entities" : qCreatedBy(id + "snapCentre", EntityType.BODY) });
        tools = append(tools, qCreatedBy(id + "snap", EntityType.BODY));

        opBoolean(context, id + "cut", {
                    "tools" : qUnion(tools),
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });

        // The rim of the slit, on the outside and the inside of the wall
        const slitMiddle = (slitLow + slitHigh) / 2;
        const slitReach = definition.slitHeight / 2 + 0.2 * millimeter;
        const slitRim = function(y is ValueWithUnits)
            {
                return qWithinRadius(qCoincidesWithPlane(qOwnedByBody(body, EntityType.EDGE), plane(vector(0 * millimeter, y, 0 * millimeter), vector(0, 1, 0))),
                        vector(0 * millimeter, y, slitMiddle), slitReach);
            };
        opChamfer(context, id + "slitChamfer", {
                    "entities" : qUnion([slitRim(front), slitRim(inner)]),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.slitChamfer });
    }, {
        "length" : 80 * millimeter,
        "width" : 28 * millimeter,
        "depth" : 20 * millimeter,
        "capHeight" : 6 * millimeter,
        "wall" : 1.8 * millimeter,
        "floor" : 2 * millimeter,
        "edgeRadius" : 7 * millimeter,
        "footChamfer" : 0.8 * millimeter,
        "footRadius" : 3 * millimeter,
        "floorChamfer" : 1.5 * millimeter,
        "buttonDiameter" : 6.7 * millimeter,
        "buttonClearance" : 0.4 * millimeter,
        "buttonCentreFromTop" : 18 * millimeter,
        "squareSize" : 12 * millimeter,
        "squareClearance" : 0.4 * millimeter,
        "squarePocket" : 0.6 * millimeter,
        "slitFromButton" : 8 * millimeter,
        "slitHeight" : 6 * millimeter,
        "slitWidth" : 1 * millimeter,
        "slitChamfer" : 0.5 * millimeter,
        "snapFromTop" : 18 * millimeter,
        "snapDepth" : 0.6 * millimeter,
        "snapDiameter" : 3.4 * millimeter
    });
