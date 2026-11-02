FeatureScript 3112;
import(path : "onshape/std/common.fs", version : "3112.0");

// The cap of the voistic stick, in place on top of the tube from the Voistic Tube feature, so both share one set of
// coordinates. Printed roof down. A skirt all around plugs into the tube and bumps on its sides snap into dimples in the
// tube. One plate runs down the back wall from the roof: full thickness to the land that presses on the USB connector and
// pushes the stack forward, then thinning to the tip. The USB window is the last cut. It runs through the roof and the
// plate down to the mouth of the connector, which leaves a rib on each side of the plate, and the bottom of the cut slopes.
// With the roof on the bed nothing needs support.
annotation { "Feature Type Name" : "Voistic Cap", "Feature Type Description" : "Cap of the voice stick with a pressing plate" }
export const voisticCap = defineFeature(function(context is Context, id is Id, definition is map)
    precondition
    {
        annotation { "Name" : "Full length, with the cap" }
        isLength(definition.length, { (millimeter) : [20, 80, 300] } as LengthBoundSpec);
        annotation { "Name" : "Width" }
        isLength(definition.width, { (millimeter) : [10, 22, 100] } as LengthBoundSpec);
        annotation { "Name" : "Depth" }
        isLength(definition.depth, { (millimeter) : [5, 18, 100] } as LengthBoundSpec);
        annotation { "Name" : "Cap height, outside the tube" }
        isLength(definition.capHeight, { (millimeter) : [3, 6, 50] } as LengthBoundSpec);
        annotation { "Name" : "Tube wall" }
        isLength(definition.wall, { (millimeter) : [0.8, 1, 5] } as LengthBoundSpec);
        annotation { "Name" : "Long edge radius" }
        isLength(definition.edgeRadius, { (millimeter) : [1, 4.5, 30] } as LengthBoundSpec);
        annotation { "Name" : "Top chamfer" }
        isLength(definition.topChamfer, { (millimeter) : [0.2, 0.8, 5] } as LengthBoundSpec);
        annotation { "Name" : "Top radius" }
        isLength(definition.topRadius, { (millimeter) : [0.2, 3, 10] } as LengthBoundSpec);

        annotation { "Name" : "Roof" }
        isLength(definition.roof, { (millimeter) : [0.8, 2, 5] } as LengthBoundSpec);
        annotation { "Name" : "Skirt depth" }
        isLength(definition.spigot, { (millimeter) : [1, 6, 20] } as LengthBoundSpec);
        annotation { "Name" : "Skirt wall" }
        isLength(definition.skirtWall, { (millimeter) : [0.5, 1, 3] } as LengthBoundSpec);
        annotation { "Name" : "Fit in the tube, on a side" }
        isLength(definition.fit, { (millimeter) : [0, 0.1, 1] } as LengthBoundSpec);

        annotation { "Name" : "Microphone board width" }
        isLength(definition.micWidth, { (millimeter) : [5, 16.5, 40] } as LengthBoundSpec);
        annotation { "Name" : "Stack, button top to USB top" }
        isLength(definition.stackThickness, { (millimeter) : [5, 14.5, 30] } as LengthBoundSpec);
        annotation { "Name" : "Button above its square" }
        isLength(definition.buttonHead, { (millimeter) : [0, 1.5, 5] } as LengthBoundSpec);
        annotation { "Name" : "Square pocket depth in the tube" }
        isLength(definition.squarePocket, { (millimeter) : [0, 0.3, 3] } as LengthBoundSpec);
        annotation { "Name" : "USB connector height" }
        isLength(definition.usbHeight, { (millimeter) : [1, 3.2, 10] } as LengthBoundSpec);
        annotation { "Name" : "USB mouth from the top of the tube" }
        isLength(definition.usbMouthFromTop, { (millimeter) : [1, 6, 60] } as LengthBoundSpec);

        annotation { "Name" : "USB window width" }
        isLength(definition.usbWindowWidth, { (millimeter) : [5, 11, 26] } as LengthBoundSpec);
        annotation { "Name" : "USB window height" }
        isLength(definition.usbWindowHeight, { (millimeter) : [3, 6, 18] } as LengthBoundSpec);
        annotation { "Name" : "USB window corner radius" }
        isLength(definition.usbWindowRadius, { (millimeter) : [0.2, 3, 10] } as LengthBoundSpec);

        annotation { "Name" : "Plate width" }
        isLength(definition.plateWidth, { (millimeter) : [4, 12.8, 24] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip thickness" }
        isLength(definition.plateTip, { (millimeter) : [0.5, 1.2, 4] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip from the top of the tube" }
        isLength(definition.plateLength, { (millimeter) : [5, 20, 70] } as LengthBoundSpec);
        annotation { "Name" : "Pressing land below the USB mouth, length" }
        isLength(definition.landLength, { (millimeter) : [0.5, 3, 20] } as LengthBoundSpec);
        annotation { "Name" : "Plate curve radius" }
        isLength(definition.plateRadius, { (millimeter) : [0.5, 10, 50] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip corner chamfer" }
        isLength(definition.tipChamfer, { (millimeter) : [0.1, 1, 5] } as LengthBoundSpec);

        annotation { "Name" : "Snap bumps from the top of the tube" }
        isLength(definition.snapFromTop, { (millimeter) : [1, 3, 70] } as LengthBoundSpec);
        annotation { "Name" : "Snap bumps height" }
        isLength(definition.snapHeight, { (millimeter) : [0.1, 0.6, 2] } as LengthBoundSpec);
        annotation { "Name" : "Snap bumps diameter" }
        isLength(definition.snapDiameter, { (millimeter) : [0.5, 4, 10] } as LengthBoundSpec);
    }
    {
        const w = definition.width;
        const d = definition.depth;
        const t = definition.wall;
        const rc = definition.edgeRadius;
        const top = definition.length;
        const h = top - definition.capHeight;
        const tubeFront = -d / 2 + t;
        const tubeBack = d / 2 - t;

        // The stack against the front wall: its square sits in the pocket, so everything else follows from that
        const squareFace = tubeFront - definition.squarePocket;
        const usbBack = squareFace - definition.buttonHead + definition.stackThickness;
        const usbY = usbBack - definition.usbHeight / 2;

        const backY = tubeBack - definition.fit;
        const windowBack = usbY + definition.usbWindowHeight / 2;
        const roofZ = top - definition.roof;
        const snapZ = h - definition.snapFromTop;
        const mouthZ = h - definition.usbMouthFromTop;
        const landBottom = mouthZ - definition.landLength;
        const tipZ = h - definition.plateLength;
        const fw = w / 2 - t - definition.fit;
        const fd = d / 2 - t - definition.fit;
        const roomW = fw - definition.skirtWall;
        const roomD = fd - definition.skirtWall;

        if (usbBack >= tubeBack)
            throw regenError("The stack does not fit in the tube", ["stackThickness"]);
        if (windowBack >= backY)
            throw regenError("The USB window leaves no root for the plate", ["usbWindowHeight"]);
        if (windowBack <= usbBack)
            throw regenError("The USB window does not reach past the connector", ["usbWindowHeight"]);
        if (definition.usbWindowRadius > definition.usbWindowHeight / 2 || definition.usbWindowRadius > definition.usbWindowWidth / 2)
            throw regenError("The window radius is too big for the window", ["usbWindowRadius"]);
        if (definition.roof >= definition.capHeight)
            throw regenError("The roof is thicker than the cap", ["roof"]);
        if (roomW <= definition.micWidth / 2)
            throw regenError("The microphone board does not fit inside the skirt", ["skirtWall"]);
        if (definition.snapFromTop + definition.snapDiameter / 2 >= definition.spigot)
            throw regenError("The snap bumps run off the skirt", ["snapFromTop"]);
        if (landBottom <= tipZ)
            throw regenError("The plate ends before the pressing land", ["plateLength"]);
        if (definition.plateTip >= backY - usbBack)
            throw regenError("The plate tip is thicker than the gap behind the USB connector", ["plateTip"]);

        // Outside: the section of the tube, closed by the same chamfer and radius as the foot
        fCuboid(context, id + "outer", {
                    "corner1" : vector(-w / 2, -d / 2, h),
                    "corner2" : vector(w / 2, d / 2, top) });
        const body = qCreatedBy(id + "outer", EntityType.BODY);
        opFillet(context, id + "longEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(body, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc });
        const topFace = qCoincidesWithPlane(qOwnedByBody(body, EntityType.FACE), plane(vector(0 * millimeter, 0 * millimeter, top), vector(0, 0, 1)));
        opChamfer(context, id + "topChamfer", {
                    "entities" : qAdjacent(topFace, AdjacencyType.EDGE, EntityType.EDGE),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.topChamfer });
        opFillet(context, id + "topRadius", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(body, EntityType.EDGE),
                            plane(vector(0 * millimeter, 0 * millimeter, top - definition.topChamfer), vector(0, 0, 1))),
                    "radius" : definition.topRadius });

        // The skirt: the inside of the tube less the fit
        fCuboid(context, id + "spigot", {
                    "corner1" : vector(-fw, -fd, h - definition.spigot),
                    "corner2" : vector(fw, fd, h + 1 * millimeter) });
        const spigot = qCreatedBy(id + "spigot", EntityType.BODY);
        opFillet(context, id + "spigotEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(spigot, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc - t - definition.fit });
        opBoolean(context, id + "join", {
                    "tools" : qUnion([body, spigot]),
                    "operationType" : BooleanOperationType.UNION });

        // Room inside the skirt, for the end of the stack
        fCuboid(context, id + "room", {
                    "corner1" : vector(-roomW, -roomD, h - definition.spigot - 1 * millimeter),
                    "corner2" : vector(roomW, roomD, roofZ) });
        const room = qCreatedBy(id + "room", EntityType.BODY);
        opFillet(context, id + "roomEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(room, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc - t - definition.fit - definition.skirtWall });
        opBoolean(context, id + "cutRoom", {
                    "tools" : room,
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });

        const yz = function(name is string, x is ValueWithUnits, width is ValueWithUnits, points is array)
            {
                const sketch = newSketchOnPlane(context, id + (name ~ "Sketch"), {
                            "sketchPlane" : plane(vector(x, 0 * millimeter, 0 * millimeter), vector(1, 0, 0), vector(0, 1, 0)) });
                skPolyline(sketch, "outline", { "points" : append(points, points[0]) });
                skSolve(sketch);
                opExtrude(context, id + name, {
                            "entities" : qSketchRegion(id + (name ~ "Sketch")),
                            "direction" : vector(1, 0, 0),
                            "endBound" : BoundingType.BLIND,
                            "endDepth" : width });
                opDeleteBodies(context, id + (name ~ "SketchGone"), { "entities" : qCreatedBy(id + (name ~ "Sketch"), EntityType.BODY) });
                return qCreatedBy(id + name, EntityType.BODY);
            };

        // The plate: from the roof to the land on the USB connector at full thickness, then down to the tip
        const pw = definition.plateWidth;
        const plate = yz("plate", -pw / 2, pw, [
                vector(backY, roofZ + 0.5 * millimeter),
                vector(usbBack, roofZ + 0.5 * millimeter),
                vector(usbBack, landBottom),
                vector(backY - definition.plateTip, tipZ),
                vector(backY, tipZ)]);
        const plateEdges = qCreatedBy(id + "plate", EntityType.EDGE);
        const tipY = backY - definition.plateTip / 2;
        opChamfer(context, id + "tipCorners", {
                    "entities" : qUnion([
                                qContainsPoint(plateEdges, vector(-pw / 2, tipY, tipZ)),
                                qContainsPoint(plateEdges, vector(pw / 2, tipY, tipZ))]),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.tipChamfer });
        opFillet(context, id + "plateRadius", {
                    "entities" : qContainsPoint(plateEdges, vector(0 * millimeter, usbBack, landBottom)),
                    "radius" : definition.plateRadius });

        // The bumps on the sides of the skirt, caps of spheres
        const snapHalf = definition.snapDiameter / 2;
        const snapR = (snapHalf * snapHalf + definition.snapHeight * definition.snapHeight) / (2 * definition.snapHeight);
        var joined = [body, plate];
        var snapSide = 0;
        for (var sx in [-1, 1])
        {
            opPoint(context, id + ("snapCentre" ~ snapSide), { "point" : vector(sx * (fw + definition.snapHeight - snapR), 0 * millimeter, snapZ) });
            fSphere(context, id + ("snap" ~ snapSide), { "center" : qCreatedBy(id + ("snapCentre" ~ snapSide), EntityType.VERTEX), "radius" : snapR });
            opDeleteBodies(context, id + ("snapCentreGone" ~ snapSide), { "entities" : qCreatedBy(id + ("snapCentre" ~ snapSide), EntityType.BODY) });
            const snap = qCreatedBy(id + ("snap" ~ snapSide), EntityType.BODY);
            const trimA = sx * (fw - 0.2 * millimeter);
            const trimB = sx * (fw - 2 * snapR - 1 * millimeter);
            fCuboid(context, id + ("snapTrim" ~ snapSide), {
                        "corner1" : vector(min(trimA, trimB), -snapR - 1 * millimeter, snapZ - snapR - 1 * millimeter),
                        "corner2" : vector(max(trimA, trimB), snapR + 1 * millimeter, snapZ + snapR + 1 * millimeter) });
            opBoolean(context, id + ("cutSnap" ~ snapSide), {
                        "tools" : qCreatedBy(id + ("snapTrim" ~ snapSide), EntityType.BODY),
                        "targets" : snap,
                        "operationType" : BooleanOperationType.SUBTRACTION });
            joined = append(joined, snap);
            snapSide += 1;
        }
        opBoolean(context, id + "joinPlate", {
                    "tools" : qUnion(joined),
                    "operationType" : BooleanOperationType.UNION });

        // Last, the USB window, built from pieces so that a radius of half the height gives a full round end
        const halfW = definition.usbWindowWidth / 2;
        const halfH = definition.usbWindowHeight / 2;
        const r = definition.usbWindowRadius;
        const cutTo = top + 1 * millimeter;
        fCuboid(context, id + "window", {
                    "corner1" : vector(-halfW + r, usbY - halfH, mouthZ),
                    "corner2" : vector(halfW - r, usbY + halfH, cutTo) });
        const window = qCreatedBy(id + "window", EntityType.BODY);
        var pieces = [window];
        if (r < halfH)
        {
            fCuboid(context, id + "windowTall", {
                        "corner1" : vector(-halfW, usbY - halfH + r, mouthZ),
                        "corner2" : vector(halfW, usbY + halfH - r, cutTo) });
            pieces = append(pieces, qCreatedBy(id + "windowTall", EntityType.BODY));
        }
        var corner = 0;
        for (var sx in [-1, 1])
        {
            for (var sy in [-1, 1])
            {
                const x = sx * (halfW - r);
                const y = usbY + sy * (halfH - r);
                fCylinder(context, id + ("windowCorner" ~ corner), {
                            "bottomCenter" : vector(x, y, mouthZ),
                            "topCenter" : vector(x, y, cutTo),
                            "radius" : r });
                pieces = append(pieces, qCreatedBy(id + ("windowCorner" ~ corner), EntityType.BODY));
                corner += 1;
            }
        }
        opBoolean(context, id + "joinWindow", {
                    "tools" : qUnion(pieces),
                    "operationType" : BooleanOperationType.UNION });
        opChamfer(context, id + "windowSlope", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(window, EntityType.EDGE), plane(vector(0 * millimeter, 0 * millimeter, mouthZ), vector(0, 0, 1))),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : windowBack - usbBack });
        opBoolean(context, id + "cutWindow", {
                    "tools" : window,
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });
    }, {
        "length" : 80 * millimeter,
        "width" : 22 * millimeter,
        "depth" : 18 * millimeter,
        "capHeight" : 6 * millimeter,
        "wall" : 1 * millimeter,
        "edgeRadius" : 4.5 * millimeter,
        "topChamfer" : 0.8 * millimeter,
        "topRadius" : 3 * millimeter,
        "roof" : 2 * millimeter,
        "spigot" : 6 * millimeter,
        "skirtWall" : 1 * millimeter,
        "fit" : 0.1 * millimeter,
        "micWidth" : 16.5 * millimeter,
        "stackThickness" : 14.5 * millimeter,
        "buttonHead" : 1.5 * millimeter,
        "squarePocket" : 0.3 * millimeter,
        "usbHeight" : 3.2 * millimeter,
        "usbMouthFromTop" : 6 * millimeter,
        "usbWindowWidth" : 11 * millimeter,
        "usbWindowHeight" : 6 * millimeter,
        "usbWindowRadius" : 3 * millimeter,
        "plateWidth" : 12.8 * millimeter,
        "plateTip" : 1.2 * millimeter,
        "plateLength" : 20 * millimeter,
        "landLength" : 3 * millimeter,
        "plateRadius" : 10 * millimeter,
        "tipChamfer" : 1 * millimeter,
        "snapFromTop" : 3 * millimeter,
        "snapHeight" : 0.6 * millimeter,
        "snapDiameter" : 4 * millimeter
    });
